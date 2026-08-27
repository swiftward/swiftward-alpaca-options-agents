# Built here rather than pulled so the allowlist and the proxy ship together and
# neither can drift from the other.
FROM alpine:3.22
RUN apk add --no-cache tinyproxy
COPY docker/egress/tinyproxy.conf /etc/tinyproxy/tinyproxy.conf
COPY docker/egress/filter.txt /etc/tinyproxy/filter.txt
# Hosts this deployment needs and the repository should not name - the address of
# its own policy gateway, for instance, which belongs to whoever runs it.
ARG EXTRA_ALLOWED_HOSTS=""
RUN if [ -n "${EXTRA_ALLOWED_HOSTS}" ]; then \
      printf '%s\n' "${EXTRA_ALLOWED_HOSTS}" >> /etc/tinyproxy/filter.txt; \
    fi
EXPOSE 8888
ENTRYPOINT ["tinyproxy", "-d", "-c", "/etc/tinyproxy/tinyproxy.conf"]
