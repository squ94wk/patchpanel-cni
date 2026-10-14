FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/dcnet ./cmd/dcnet \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/dcnet-agent ./cmd/dcnet-agent

FROM alpine:3.22
COPY --from=build /out/dcnet /out/dcnet-agent /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/dcnet-agent"]
