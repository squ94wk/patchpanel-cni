FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/patchpanel ./cmd/patchpanel \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/patchpanel-agent ./cmd/patchpanel-agent

FROM alpine:3.22
COPY --from=build /out/patchpanel /out/patchpanel-agent /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/patchpanel-agent"]
