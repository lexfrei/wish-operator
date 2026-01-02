// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025 Aleksei Sviridkin

package i18n

// messages contains all translations keyed by language code.
//
//nolint:gochecknoglobals,gosmopolitan // immutable translation map with CJK characters
var messages = map[string]map[string]string{
	LangEN: {
		// UI strings
		"page_title":      "Wishlist",
		"filter_label":    "Filter:",
		"filter_all":      "All",
		"empty_filtered":  "No wishes with tag",
		"empty_default":   "No wishes yet.",
		"buy_label":       "Buy:",
		"reserved_badge":  "Reserved",
		"reserved_until":  "until %s",
		"reserve_btn":     "Reserve",
		"weeks_format":    "weeks",
		"week_one":        "week",
		"quantity_label":  "Qty:",
		"available_label": "Available:",
		"reserved_count":  "%d reserved until %s",

		// Error messages
		"err_list_wishes":      "Failed to list wishes",
		"err_render":           "Failed to render template",
		"err_missing_name":     "Missing wish name",
		"err_invalid_form":     "Invalid form data",
		"err_weeks_range":      "Weeks must be between %d and %d",
		"err_not_found":        "Wish not found",
		"err_get_wish":         "Failed to get wish",
		"err_already_reserved": "Wish is already reserved",
		"err_reserve_failed":   "Failed to reserve wish",
		"err_rate_limit":       "Too many requests",
		"err_invalid_quantity": "Invalid quantity",
		"err_fully_reserved":   "All items are reserved",
		"err_quantity_exceeds": "Only %d available",
	},
	LangRU: {
		// UI strings
		"page_title":      "Список желаний",
		"filter_label":    "Фильтр:",
		"filter_all":      "Все",
		"empty_filtered":  "Нет желаний с тегом",
		"empty_default":   "Пока нет желаний.",
		"buy_label":       "Купить:",
		"reserved_badge":  "Зарезервировано",
		"reserved_until":  "до %s",
		"reserve_btn":     "Зарезервировать",
		"week_one":        "неделя",
		"weeks_few":       "недели",
		"weeks_many":      "недель",
		"weeks_format":    "недель",
		"quantity_label":  "Кол-во:",
		"available_label": "Доступно:",
		"reserved_count":  "%d зарезервировано до %s",

		// Error messages
		"err_list_wishes":      "Не удалось загрузить список желаний",
		"err_render":           "Ошибка отображения",
		"err_missing_name":     "Не указано название",
		"err_invalid_form":     "Неверные данные формы",
		"err_weeks_range":      "Срок должен быть от %d до %d недель",
		"err_not_found":        "Желание не найдено",
		"err_get_wish":         "Не удалось получить желание",
		"err_already_reserved": "Уже зарезервировано",
		"err_reserve_failed":   "Не удалось зарезервировать",
		"err_rate_limit":       "Слишком много запросов",
		"err_invalid_quantity": "Неверное количество",
		"err_fully_reserved":   "Всё зарезервировано",
		"err_quantity_exceeds": "Доступно только %d",
	},
	LangZH: {
		// UI strings
		"page_title":      "愿望清单",
		"filter_label":    "筛选：",
		"filter_all":      "全部",
		"empty_filtered":  "没有带有此标签的愿望",
		"empty_default":   "暂无愿望",
		"buy_label":       "购买：",
		"reserved_badge":  "已预订",
		"reserved_until":  "至 %s",
		"reserve_btn":     "预订",
		"weeks_format":    "周",
		"week_one":        "周",
		"quantity_label":  "数量：",
		"available_label": "可用：",
		"reserved_count":  "%d 已预订至 %s",

		// Error messages
		"err_list_wishes":      "无法加载愿望列表",
		"err_render":           "渲染失败",
		"err_missing_name":     "缺少名称",
		"err_invalid_form":     "表单数据无效",
		"err_weeks_range":      "周数必须在%d到%d之间",
		"err_not_found":        "未找到愿望",
		"err_get_wish":         "获取愿望失败",
		"err_already_reserved": "已被预订",
		"err_reserve_failed":   "预订失败",
		"err_rate_limit":       "请求过多",
		"err_invalid_quantity": "数量无效",
		"err_fully_reserved":   "全部已预订",
		"err_quantity_exceeds": "仅有 %d 件可用",
	},
}
