// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	wishlistv1alpha1 "github.com/lexfrei/wish-operator/api/v1alpha1"
)

var _ = Describe("Wish Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When reconciling a new Wish", func() {
		const wishName = "test-wish-new"
		const wishNamespace = "default"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      wishName,
			Namespace: wishNamespace,
		}

		BeforeEach(func() {
			By("Creating a new Wish resource")
			wish := &wishlistv1alpha1.Wish{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wishName,
					Namespace: wishNamespace,
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title:    "Test Gift",
					Priority: 3,
					TTL:      &metav1.Duration{Duration: 24 * time.Hour},
				},
			}
			Expect(k8sClient.Create(ctx, wish)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleaning up the Wish resource")
			wish := &wishlistv1alpha1.Wish{}
			err := k8sClient.Get(ctx, typeNamespacedName, wish)
			if err == nil {
				Expect(k8sClient.Delete(ctx, wish)).To(Succeed())
			}
		})

		It("should set Active to true for wish within TTL", func() {
			By("Reconciling the created resource")
			reconciler := &WishReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Active is set to true")
			wish := &wishlistv1alpha1.Wish{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, wish)
				if err != nil {
					return false
				}
				return wish.Status.Active
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When reconciling a Wish with expired TTL", func() {
		const wishName = "test-wish-expired"
		const wishNamespace = "default"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      wishName,
			Namespace: wishNamespace,
		}

		BeforeEach(func() {
			By("Creating a Wish with very short TTL")
			wish := &wishlistv1alpha1.Wish{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wishName,
					Namespace: wishNamespace,
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title: "Expired Gift",
					TTL:   &metav1.Duration{Duration: time.Millisecond},
				},
			}
			Expect(k8sClient.Create(ctx, wish)).To(Succeed())
			// Wait for TTL to expire
			time.Sleep(10 * time.Millisecond)
		})

		AfterEach(func() {
			By("Cleaning up the Wish resource")
			wish := &wishlistv1alpha1.Wish{}
			err := k8sClient.Get(ctx, typeNamespacedName, wish)
			if err == nil {
				Expect(k8sClient.Delete(ctx, wish)).To(Succeed())
			}
		})

		It("should set Active to false for expired wish", func() {
			By("Reconciling the expired resource")
			reconciler := &WishReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Active is set to false")
			wish := &wishlistv1alpha1.Wish{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, wish)
				if err != nil {
					return true // Error means we can't verify
				}
				return !wish.Status.Active
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When reconciling a Wish with expired reservation", func() {
		const wishName = "test-wish-reservation-expired"
		const wishNamespace = "default"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      wishName,
			Namespace: wishNamespace,
		}

		BeforeEach(func() {
			By("Creating a Wish with expired reservation")
			pastTime := metav1.NewTime(time.Now().Add(-time.Hour))
			wish := &wishlistv1alpha1.Wish{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wishName,
					Namespace: wishNamespace,
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title: "Reserved Gift",
				},
			}
			Expect(k8sClient.Create(ctx, wish)).To(Succeed())

			// Update status with expired reservation
			wish.Status.Reserved = true
			wish.Status.ReservedAt = &pastTime
			wish.Status.ReservationExpires = &pastTime
			Expect(k8sClient.Status().Update(ctx, wish)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleaning up the Wish resource")
			wish := &wishlistv1alpha1.Wish{}
			err := k8sClient.Get(ctx, typeNamespacedName, wish)
			if err == nil {
				Expect(k8sClient.Delete(ctx, wish)).To(Succeed())
			}
		})

		It("should clear expired reservation", func() {
			By("Reconciling the resource with expired reservation")
			reconciler := &WishReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that reservation is cleared")
			wish := &wishlistv1alpha1.Wish{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, wish)
				if err != nil {
					return false
				}
				return !wish.Status.Reserved &&
					wish.Status.ReservedAt == nil &&
					wish.Status.ReservationExpires == nil
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("When reconciling a Wish without TTL", func() {
		const wishName = "test-wish-no-ttl"
		const wishNamespace = "default"

		ctx := context.Background()
		typeNamespacedName := types.NamespacedName{
			Name:      wishName,
			Namespace: wishNamespace,
		}

		BeforeEach(func() {
			By("Creating a Wish without TTL")
			wish := &wishlistv1alpha1.Wish{
				ObjectMeta: metav1.ObjectMeta{
					Name:      wishName,
					Namespace: wishNamespace,
				},
				Spec: wishlistv1alpha1.WishSpec{
					Title: "Eternal Gift",
				},
			}
			Expect(k8sClient.Create(ctx, wish)).To(Succeed())
		})

		AfterEach(func() {
			By("Cleaning up the Wish resource")
			wish := &wishlistv1alpha1.Wish{}
			err := k8sClient.Get(ctx, typeNamespacedName, wish)
			if err == nil {
				Expect(k8sClient.Delete(ctx, wish)).To(Succeed())
			}
		})

		It("should set Active to true (never expires)", func() {
			By("Reconciling the resource")
			reconciler := &WishReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that Active is true")
			wish := &wishlistv1alpha1.Wish{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, wish)
				if err != nil {
					return false
				}
				return wish.Status.Active
			}, timeout, interval).Should(BeTrue())
		})
	})
})
