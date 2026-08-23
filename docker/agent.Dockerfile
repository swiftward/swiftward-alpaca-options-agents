# Образ, в котором живёт торговая сессия: планировщик на Go плюс агент,
# запускаемый как отдельный процесс с доступом к MCP-серверу Alpaca через шлюз.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY golang/go.mod golang/go.sum* ./
RUN go mod download
COPY golang/ ./
RUN CGO_ENABLED=0 go build -o /out/runner ./apps/runner

FROM node:22-alpine
RUN npm install -g @anthropic-ai/claude-code
COPY --from=build /out/runner /usr/local/bin/runner
COPY agent/ /agent/
COPY playbooks/ /playbooks/
ENTRYPOINT ["/usr/local/bin/runner"]
