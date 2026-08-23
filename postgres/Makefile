.PHONY: migrate

# Миграции накатываются по порядку имён файлов. Каждая - только добавление:
# существующие таблицы и колонки не переименовываются и не удаляются.
migrate:
	@for f in migrations/*.sql; do echo "-> $$f"; psql "$$DATABASE_URL" -v ON_ERROR_STOP=1 -f "$$f"; done
