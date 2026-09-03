FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /out/plain-text-parser .

FROM scratch
COPY --from=build /out/plain-text-parser /plain-text-parser
USER 65532:65532
ENTRYPOINT ["/plain-text-parser"]
