FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY services ./services
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kinga ./services/gateway/cmd
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/kinga /kinga
COPY testdata/telco/simulator.json /app/testdata/telco/simulator.json
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/kinga"]
