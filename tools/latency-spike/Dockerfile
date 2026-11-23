FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY tools/latency-spike/stub-scorer ./tools/latency-spike/stub-scorer
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/stub-scorer ./tools/latency-spike/stub-scorer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/stub-scorer /stub-scorer
USER nonroot:nonroot
EXPOSE 8092
ENTRYPOINT ["/stub-scorer"]
