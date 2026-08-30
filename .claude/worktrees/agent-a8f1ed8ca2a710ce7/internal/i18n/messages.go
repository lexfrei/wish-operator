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
	keyReservedBadge      = "reserved_badge"
	keyReservedUntil      = "reserved_until"
	keyReserveBtn         = "reserve_btn"
	keyWeeksFormat        = "weeks_format"
	keyWeekOne            = "week_one"
	keyWeeksFew           = "weeks_few"
	keyWeeksMany          = "weeks_many"
	keyQuantityLabel      = "quantity_label"
	keyAvailableLabel     = "available_label"
	keyUnlimitedLabel     = "unlimited_label"
	keyUnlimitedAvailable = "unlimited_available"
	keyReservedCount      = "reserved_count"

	keyErrListWishes      = "err_list_wishes"
	keyErrRender          = "err_render"
	keyErrMissingName     = "err_missing_name"
	keyErrInvalidForm     = "err_invalid_form"
	keyErrWeeksRange      = "err_weeks_range"
	keyErrNotFound        = "err_not_found"
	keyErrGetWish         = "err_get_wish"
	keyErrAlreadyReserved = "err_already_reserved"
	keyErrReserveFailed   = "err_reserve_failed"
	keyErrRateLimit       = "err_rate_limit"
	keyErrInvalidQuantity = "err_invalid_quantity"
	keyErrFullyReserved   = "err_fully_reserved"
	keyErrQuantityExceeds = "err_quantity_exceeds"
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
		keyReservedBadge:      "Reserved",
		keyReservedUntil:      "until %s",
		keyReserveBtn:         "Reserve",
		keyWeeksFormat:        "weeks",
		keyWeekOne:            "week",
		keyQuantityLabel:      "Qty:",
		keyAvailableLabel:     "Available:",
		keyUnlimitedLabel:     "Unlimited",
		keyUnlimitedAvailable: "Available: ∞",
		keyReservedCount:      "%d reserved until %s",

		// Error messages
		keyErrListWishes:      "Failed to list wishes",
		keyErrRender:          "Failed to render template",
		keyErrMissingName:     "Missing wish name",
		keyErrInvalidForm:     "Invalid form data",
		keyErrWeeksRange:      "Weeks must be between %d and %d",
		keyErrNotFound:        "Wish not found",
		keyErrGetWish:         "Failed to get wish",
		keyErrAlreadyReserved: "Wish is already reserved",
		keyErrReserveFailed:   "Failed to reserve wish",
		keyErrRateLimit:       "Too many requests",
		keyErrInvalidQuantity: "Invalid quantity",
		keyErrFullyReserved:   "All items are reserved",
		keyErrQuantityExceeds: "Only %d available",
	},
	LangRU: {
		// UI strings
		keyPageTitle:          "Список желаний",
		keyFilterLabel:        "Фильтр:",
		keyFilterAll:          "Все",
		keyEmptyFiltered:      "Нет желаний с тегом",
		keyEmptyDefault:       "Пока нет желаний.",
		keyBuyLabel:           "Купить:",
		keyReservedBadge:      "Зарезервировано",
		keyReservedUntil:      "до %s",
		keyReserveBtn:         "Зарезервировать",
		keyWeekOne:            "неделя",
		keyWeeksFew:           "недели",
		keyWeeksMany:          ruWeeksMany,
		keyWeeksFormat:        ruWeeksMany,
		keyQuantityLabel:      "Кол-во:",
		keyAvailableLabel:     "Доступно:",
		keyUnlimitedLabel:     "Неограничено",
		keyUnlimitedAvailable: "Доступно: ∞",
		keyReservedCount:      "%d зарезервировано до %s",

		// Error messages
		keyErrListWishes:      "Не удалось загрузить список желаний",
		keyErrRender:          "Ошибка отображения",
		keyErrMissingName:     "Не указано название",
		keyErrInvalidForm:     "Неверные данные формы",
		keyErrWeeksRange:      "Срок должен быть от %d до %d недель",
		keyErrNotFound:        "Желание не найдено",
		keyErrGetWish:         "Не удалось получить желание",
		keyErrAlreadyReserved: "Уже зарезервировано",
		keyErrReserveFailed:   "Не удалось зарезервировать",
		keyErrRateLimit:       "Слишком много запросов",
		keyErrInvalidQuantity: "Неверное количество",
		keyErrFullyReserved:   "Всё зарезервировано",
		keyErrQuantityExceeds: "Доступно только %d",
	},
	LangZH: {
		// UI strings
		keyPageTitle:          "愿望清单",
		keyFilterLabel:        "筛选：",
		keyFilterAll:          "全部",
		keyEmptyFiltered:      "没有带有此标签的愿望",
		keyEmptyDefault:       "暂无愿望",
		keyBuyLabel:           "购买：",
		keyReservedBadge:      "已预订",
		keyReservedUntil:      "至 %s",
		keyReserveBtn:         "预订",
		keyWeeksFormat:        "周",
		keyWeekOne:            "周",
		keyQuantityLabel:      "数量：",
		keyAvailableLabel:     "可用：",
		keyUnlimitedLabel:     "无限",
		keyUnlimitedAvailable: "可用：∞",
		keyReservedCount:      "%d 已预订至 %s",

		// Error messages
		keyErrListWishes:      "无法加载愿望列表",
		keyErrRender:          "渲染失败",
		keyErrMissingName:     "缺少名称",
		keyErrInvalidForm:     "表单数据无效",
		keyErrWeeksRange:      "周数必须在%d到%d之间",
		keyErrNotFound:        "未找到愿望",
		keyErrGetWish:         "获取愿望失败",
		keyErrAlreadyReserved: "已被预订",
		keyErrReserveFailed:   "预订失败",
		keyErrRateLimit:       "请求过多",
		keyErrInvalidQuantity: "数量无效",
		keyErrFullyReserved:   "全部已预订",
		keyErrQuantityExceeds: "仅有 %d 件可用",
	},
}
