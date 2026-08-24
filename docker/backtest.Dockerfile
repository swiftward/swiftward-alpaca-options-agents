# The backtester is a command, not a service: it reads a strategy declaration and
# a slice of history, prints what the run produced, and exits.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY golang/go.mod golang/go.sum ./
RUN go mod download
COPY golang/ ./
RUN CGO_ENABLED=0 go build -o /out/backtest ./apps/backtest

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/backtest /backtest
USER nonroot:nonroot
ENTRYPOINT ["/backtest"]
