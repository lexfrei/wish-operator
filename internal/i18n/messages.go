// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package i18n

// Translation keys. Shared across every language map below so they are
// declared once here to stay typo-safe and avoid duplicated string literals.
const (
	keyPageTitle          = "page_title"
	keyFilterLabel        = "filter_label"
	keyFilterAll          = "filter_all"
	keyEmptyFiltered      = "empty_filtered"
	keyEmptyDefault       = "empty_default"
	keyBuyLabel           = "buy_label"
	keyReserveBtn         = "reserve_btn"
	keyWeeksFormat        = "weeks_format"
	keyWeekOne            = "week_one"
	keyWeeksFew           = "weeks_few"
	keyWeeksMany          = "weeks_many"
	keyAvailableLabel     = "available_label"
	keyUnlimitedAvailable = "unlimited_available"
	keyReservedCount      = "reserved_count"

	keyErrListWishes         = "err_list_wishes"
	keyErrRender             = "err_render"
	keyErrMissingName        = "err_missing_name"
	keyErrInvalidForm        = "err_invalid_form"
	keyErrWeeksRange         = "err_weeks_range"
	keyErrNotFound           = "err_not_found"
	keyErrGetWish            = "err_get_wish"
	keyErrReserveFailed      = "err_reserve_failed"
	keyErrRateLimit          = "err_rate_limit"
	keyErrInvalidQuantity    = "err_invalid_quantity"
	keyErrFullyReserved      = "err_fully_reserved"
	keyErrQuantityExceeds    = "err_quantity_exceeds"
	keyErrReservationLimit   = "err_reservation_limit"
	keyErrQuantityPerRequest = "err_quantity_per_request"
	keyErrReserveBusy        = "err_reserve_busy"
	keyErrNoResponse         = "err_no_response"
)

// ruWeeksMany is the Russian plural form for "weeks", shared by the
// weeks_many and weeks_format keys.
const ruWeeksMany = "недель"

// messages contains all translations keyed by language code.
//
//nolint:gochecknoglobals,gosmopolitan // immutable translation map with CJK characters
var messages = map[string]map[string]string{
	LangEN: {
		// UI strings
		keyPageTitle:          "Wishlist",
		keyFilterLabel:        "Filter:",
		keyFilterAll:          "All",
		keyEmptyFiltered:      "No wishes with tag",
		keyEmptyDefault:       "No wishes yet.",
		keyBuyLabel:           "Buy:",
		keyReserveBtn:         "Reserve",
		keyWeeksFormat:        "weeks",
		keyWeekOne:            "week",
		keyAvailableLabel:     "Available:",
		keyUnlimitedAvailable: "Available: ∞",
		keyReservedCount:      "%d reserved until %s",

		// Error messages
		keyErrListWishes:         "Failed to list wishes",
		keyErrRender:             "Failed to render template",
		keyErrMissingName:        "Missing wish name",
		keyErrInvalidForm:        "Invalid form data",
		keyErrWeeksRange:         "Weeks must be between %d and %d",
		keyErrNotFound:           "Wish not found",
		keyErrGetWish:            "Failed to get wish",
		keyErrReserveFailed:      "Failed to reserve wish",
		keyErrRateLimit:          "Too many requests",
		keyErrInvalidQuantity:    "Invalid quantity",
		keyErrFullyReserved:      "All items are reserved",
		keyErrQuantityExceeds:    "Only %d available",
		keyErrReservationLimit:   "This wish cannot take any more reservations right now",
		keyErrQuantityPerRequest: "At most %d per reservation",
		keyErrReserveBusy:        "Someone else was reserving this at the same time. Try again.",
		keyErrNoResponse:         "The request did not reach the server. Check your connection and try again.",
	},
	LangRU: {
		// UI strings
		keyPageTitle:          "Список желаний",
		keyFilterLabel:        "Фильтр:",
		keyFilterAll:          "Все",
		keyEmptyFiltered:      "Нет желаний с тегом",
		keyEmptyDefault:       "Пока нет желаний.",
		keyBuyLabel:           "Купить:",
		keyReserveBtn:         "Зарезервировать",
		keyWeekOne:            "неделя",
		keyWeeksFew:           "недели",
		keyWeeksMany:          ruWeeksMany,
		keyWeeksFormat:        ruWeeksMany,
		keyAvailableLabel:     "Доступно:",
		keyUnlimitedAvailable: "Доступно: ∞",
		keyReservedCount:      "%d зарезервировано до %s",

		// Error messages
		keyErrListWishes:         "Не удалось загрузить список желаний",
		keyErrRender:             "Ошибка отображения",
		keyErrMissingName:        "Не указано название",
		keyErrInvalidForm:        "Неверные данные формы",
		keyErrWeeksRange:         "Срок должен быть от %d до %d недель",
		keyErrNotFound:           "Желание не найдено",
		keyErrGetWish:            "Не удалось получить желание",
		keyErrReserveFailed:      "Не удалось зарезервировать",
		keyErrRateLimit:          "Слишком много запросов",
		keyErrInvalidQuantity:    "Неверное количество",
		keyErrFullyReserved:      "Всё зарезервировано",
		keyErrQuantityExceeds:    "Доступно только %d",
		keyErrReservationLimit:   "Это желание пока не может принять больше броней",
		keyErrQuantityPerRequest: "Не больше %d за одну бронь",
		keyErrReserveBusy:        "Кто-то бронировал это одновременно с вами. Попробуйте ещё раз.",
		keyErrNoResponse:         "Запрос не дошёл до сервера. Проверьте соединение и попробуйте ещё раз.",
	},
	LangZH: {
		// UI strings
		keyPageTitle:          "愿望清单",
		keyFilterLabel:        "筛选：",
		keyFilterAll:          "全部",
		keyEmptyFiltered:      "没有带有此标签的愿望",
		keyEmptyDefault:       "暂无愿望",
		keyBuyLabel:           "购买：",
		keyReserveBtn:         "预订",
		keyWeeksFormat:        "周",
		keyWeekOne:            "周",
		keyAvailableLabel:     "可用：",
		keyUnlimitedAvailable: "可用：∞",
		keyReservedCount:      "%d 已预订至 %s",

		// Error messages
		keyErrListWishes:         "无法加载愿望列表",
		keyErrRender:             "渲染失败",
		keyErrMissingName:        "缺少名称",
		keyErrInvalidForm:        "表单数据无效",
		keyErrWeeksRange:         "周数必须在%d到%d之间",
		keyErrNotFound:           "未找到愿望",
		keyErrGetWish:            "获取愿望失败",
		keyErrReserveFailed:      "预订失败",
		keyErrRateLimit:          "请求过多",
		keyErrInvalidQuantity:    "数量无效",
		keyErrFullyReserved:      "全部已预订",
		keyErrQuantityExceeds:    "仅有 %d 件可用",
		keyErrReservationLimit:   "该愿望暂时无法接受更多预订",
		keyErrQuantityPerRequest: "每次预订最多 %d 件",
		keyErrReserveBusy:        "有人同时在预订这件愿望，请再试一次。",
		keyErrNoResponse:         "请求未送达服务器，请检查网络后重试。",
	},
}
