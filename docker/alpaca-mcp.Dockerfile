# Alpaca publishes no image, so this is their released package pinned to a
# version. The account keys are this container's environment and reach nothing
# else in the stack.
FROM python:3.13-alpine

COPY docker/alpaca-mcp.requirements.txt /tmp/requirements.txt
RUN pip install --no-cache-dir -r /tmp/requirements.txt && rm /tmp/requirements.txt

RUN adduser -D -u 1002 mcp
USER mcp

EXPOSE 8000
ENTRYPOINT ["alpaca-mcp-server", "serve", "--transport", "streamable-http", "--host", "0.0.0.0", "--port", "8000"]
