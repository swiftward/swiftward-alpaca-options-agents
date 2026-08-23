# Один Dockerfile на все приложения Go: APP выбирает, какое собрать.
FROM golang:1.27-alpine AS build
ARG APP
WORKDIR /src
COPY golang/go.mod golang/go.sum* ./
RUN go mod download
COPY golang/ ./
RUN CGO_ENABLED=0 go build -o /out/app ./apps/${APP}

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
