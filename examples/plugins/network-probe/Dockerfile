FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
ARG GOPROXY=https://proxy.golang.org,direct
RUN GOPROXY=$GOPROXY go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/network-probe .

FROM scratch
COPY --from=build /out/network-probe /network-probe
USER 65532:65532
ENTRYPOINT ["/network-probe"]
