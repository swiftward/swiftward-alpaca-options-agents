# Built here rather than pulled so the allowlist and the proxy ship together and
# neither can drift from the other.
FROM alpine:3.22
RUN apk add --no-cache tinyproxy
COPY docker/egress/tinyproxy.conf /etc/tinyproxy/tinyproxy.conf
COPY docker/egress/filter.txt /etc/tinyproxy/filter.txt
EXPOSE 8888
ENTRYPOINT ["tinyproxy", "-d", "-c", "/etc/tinyproxy/tinyproxy.conf"]
