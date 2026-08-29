# The schema ships with the code rather than beside it.
#
# The migrations used to be mounted from the disk of whichever machine brought the
# stack up. So what was applied came from the checkout while the code came from
# the image, and the two matched only while somebody watched. Now it is one
# delivery: the schema image is built from the same commit as the rest and carries
# the same tag.
FROM postgres:18.4-alpine

COPY postgres/migrations /migrations
COPY postgres/migrate.sh /usr/local/bin/migrate
RUN chmod +x /usr/local/bin/migrate

ENTRYPOINT ["/usr/local/bin/migrate"]
