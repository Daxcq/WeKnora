FROM golang:1.26-bookworm AS build
WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
COPY go.mod go.sum ./
RUN GOPROXY=$GOPROXY go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/plain-text-parser .

FROM scratch
COPY --from=build /out/plain-text-parser /plain-text-parser
USER 65532:65532
ENTRYPOINT ["/plain-text-parser"]
