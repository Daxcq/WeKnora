FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/github .

FROM scratch
COPY --from=build /out/github /github
USER 65532:65532
ENTRYPOINT ["/github"]
