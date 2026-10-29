FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
ARG APP_VERSION=development
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -trimpath -buildvcs=false -ldflags="-s -w -X main.buildVersion=${APP_VERSION}" -o /out/release-board .

FROM scratch
COPY --from=build /out/release-board /release-board
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/release-board"]
