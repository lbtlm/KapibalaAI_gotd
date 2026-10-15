FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./cmd/migrate && CGO_ENABLED=0 go build -trimpath -o /out/agent ./cmd/agent
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app && mkdir -p /app/media && chown app:app /app/media
COPY --from=build /out/ /app/
USER app
WORKDIR /app
ENV BIND_HOST=0.0.0.0
EXPOSE 8080 8082
CMD ["/app/server"]
