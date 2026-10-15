FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /adopt ./cmd/adopt

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gateway /gateway
COPY --from=build /adopt /adopt
ENV PORT=8081 BIND_HOST=0.0.0.0 GATEWAY_MODE=telegram
EXPOSE 8081
ENTRYPOINT ["/gateway"]
