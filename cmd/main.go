// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
	"github.com/lexfrei/wish-operator/internal/controller"
	"github.com/lexfrei/wish-operator/internal/web"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// Version and GitSHA are set at build time via -ldflags. The Containerfile
	// references them by these exact paths, so renaming either one silently
	// drops the value instead of failing the build.
	Version = "development"
	GitSHA  = "development"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(wishlistv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

const (
	// webReadHeaderTimeout bounds how long a caller may take to send headers.
	webReadHeaderTimeout = 10 * time.Second
	// webShutdownTimeout bounds how long in-flight requests get to finish.
	webShutdownTimeout = 5 * time.Second
)

// webOptions groups the settings of the public wishlist web server.
type webOptions struct {
	namespace        string
	trustedProxyHops int
	rateLimit        float64
	rateBurst        int
}

// bindWebFlags registers the web server flags on flagSet.
func bindWebFlags(flagSet *flag.FlagSet, opts *webOptions) {
	flagSet.StringVar(&opts.namespace, "web-namespace", "default",
		"The namespace whose Wishes the web UI lists and reserves. "+
			"The controller reconciles Wishes in all namespaces regardless of this.")
	flagSet.IntVar(&opts.trustedProxyHops, "trusted-proxy-hops", 0,
		"Number of proxies in front of the web server that append to X-Forwarded-For. "+
			"Rate limiting reads the client address from that many entries counted from the right. "+
			"Leave at 0 when the server is reachable directly, so proxy headers are ignored.")
	flagSet.Float64Var(&opts.rateLimit, "rate-limit", 30, "Rate limit requests per second per IP.")
	flagSet.IntVar(&opts.rateBurst, "rate-burst", 10, "Rate limit burst size.")
}

// Errors returned by validateWebFlags.
var (
	errEmptyNamespace    = errors.New("--web-namespace must not be empty")
	errNegativeProxyHops = errors.New("--trusted-proxy-hops must not be negative")
	errRateLimitInvalid  = errors.New("--rate-limit must be a finite number greater than zero")
	errRateBurstTooLow   = errors.New("--rate-burst must be at least one")
)

// newWebServer maps parsed flags onto the web server. web.NewServer takes its
// arguments positionally and two of them are ints, so a transposition compiles
// and runs; this is the single place that mapping is written down, and the
// place a test can pin it.
func newWebServer(c client.Client, opts *webOptions) *web.Server {
	return web.NewServer(c, opts.namespace, opts.trustedProxyHops, opts.rateLimit, opts.rateBurst)
}

// validateWebFlags rejects settings the flag package cannot express. Each one
// is a value the binary would otherwise accept and then misbehave on: a
// negative hop count reads like "trust one proxy" but disables proxy headers,
// and a zero rate or burst refuses every single request with 429 rather than
// limiting anything.
//
// Non-finite rates matter most, because they are the only ones that fail open.
// NaN passes every ordered comparison, so it survives a bare "<= 0" check and
// then makes the limiter allow everything; +Inf is rate.Inf, which is
// unlimited by definition. Both turn rate limiting off with nothing in the log
// to say so, and neither is a rate anybody means to ask for.
func validateWebFlags(opts *webOptions) error {
	// An empty namespace is not a narrower scope, it is every namespace: the
	// client treats it as cluster-wide on list, and the public page would serve
	// every Wish in the cluster while every reservation failed to resolve.
	if opts.namespace == "" {
		return errEmptyNamespace
	}

	if opts.trustedProxyHops < 0 {
		return fmt.Errorf("%w, got %d", errNegativeProxyHops, opts.trustedProxyHops)
	}

	if math.IsNaN(opts.rateLimit) || math.IsInf(opts.rateLimit, 0) || opts.rateLimit <= 0 {
		return fmt.Errorf("%w, got %v", errRateLimitInvalid, opts.rateLimit)
	}

	if opts.rateBurst < 1 {
		return fmt.Errorf("%w, got %d", errRateBurstTooLow, opts.rateBurst)
	}

	return nil
}

func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var webAddr string

	var webOpts webOptions

	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&webAddr, "web-bind-address", ":8080", "The address the web server binds to.")
	bindWebFlags(flag.CommandLine, &webOpts)
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info("build info", "version", Version, "gitSHA", GitSHA)

	if err := validateWebFlags(&webOpts); err != nil {
		setupLog.Error(err, "invalid web server flags")
		os.Exit(1)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.4/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "b1249f94.k8s.lex.la",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.WishReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Wish")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// Start web server
	webServer := newWebServer(mgr.GetClient(), &webOpts)
	if err := mgr.Add(&webRunnable{addr: webAddr, handler: webServer.Handler()}); err != nil {
		setupLog.Error(err, "unable to add web server")
		os.Exit(1)
	}
	setupLog.Info("web server configured",
		"address", webAddr,
		"namespace", webOpts.namespace,
		"trustedProxyHops", webOpts.trustedProxyHops)

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// webRunnable implements manager.Runnable for the web server.
type webRunnable struct {
	addr    string
	handler http.Handler
	// shutdownTimeout overrides webShutdownTimeout; zero means the default.
	shutdownTimeout time.Duration
}

// NeedLeaderElection keeps the web server out of the leader-election group so it
// answers on every replica. With leader election enabled, runnables that do not
// implement this interface start only on the leader, which would leave the Service
// routing to pods with nothing listening on the web port. With it disabled every
// runnable starts everywhere, so this only matters once leader election is on.
func (w *webRunnable) NeedLeaderElection() bool {
	return false
}

func (w *webRunnable) Start(ctx context.Context) error {
	server := &http.Server{
		Addr:              w.addr,
		Handler:           w.handler,
		ReadHeaderTimeout: webReadHeaderTimeout,
	}

	drained := make(chan struct{})

	timeout := w.shutdownTimeout
	if timeout == 0 {
		timeout = webShutdownTimeout
	}

	go func() {
		defer close(drained)

		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()

		// A timeout here means in-flight responses were cut mid-write. The
		// runnable still stops cleanly, so without this line the only trace of
		// a truncated rollout would be on the client side.
		if err := server.Shutdown(shutdownCtx); err != nil {
			setupLog.Error(err, "web server shutdown did not drain in time", "timeout", timeout)
		}
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	// Shutdown closes the listeners first, so ListenAndServe returns while
	// requests are still being served. Returning here would let the manager
	// count this runnable as stopped and the process exit mid-response, which
	// would make the shutdown timeout above mean nothing.
	<-drained

	return nil
}
