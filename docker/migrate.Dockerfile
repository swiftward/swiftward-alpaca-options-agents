# Схема едет вместе с кодом, а не рядом с ним.
#
# Раньше миграции монтировались с диска той машины, где поднимается стек. Значит
# накатывалось то, что лежит в чекауте, а работал код из образа - и совпадали они
# только пока кто-то за этим следил. Теперь это одна поставка: образ схемы собран
# из того же коммита, что и остальные, и метка у него та же.
FROM postgres:18.4-alpine

COPY postgres/migrations /migrations
COPY postgres/migrate.sh /usr/local/bin/migrate
RUN chmod +x /usr/local/bin/migrate

ENTRYPOINT ["/usr/local/bin/migrate"]
