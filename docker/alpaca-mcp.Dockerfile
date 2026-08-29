# Alpaca publishes no image, so this is their released package pinned to a
# version. The account keys are this container's environment and reach nothing
# else in the stack.
FROM python:3.13-alpine

ARG ALPACA_MCP_VERSION=2.3.0
RUN pip install --no-cache-dir "alpaca-mcp-server==${ALPACA_MCP_VERSION}"

RUN adduser -D -u 1002 mcp
USER mcp

EXPOSE 8000
ENTRYPOINT ["alpaca-mcp-server", "serve", "--transport", "streamable-http", "--host", "0.0.0.0", "--port", "8000"]
