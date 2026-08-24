# The three roles in one image, with the built page inside it: the api role
# serves that directory, and the same build is what goes to the demo host.
FROM node:22-alpine AS web
WORKDIR /src
COPY typescript/web/package*.json ./
RUN npm ci
COPY typescript/web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY golang/go.mod golang/go.sum ./
RUN go mod download
COPY golang/ ./
RUN CGO_ENABLED=0 go build -o /out/app ./apps/app

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/app /app
COPY --from=web /src/dist /srv/web
USER nonroot:nonroot
ENTRYPOINT ["/app"]
