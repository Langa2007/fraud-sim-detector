FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY services ./services
COPY tools/health-probe ./tools/health-probe
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kinga ./services/gateway/cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/health-probe ./tools/health-probe
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/kinga /kinga
COPY --from=build /out/health-probe /health-probe
COPY testdata/telco/simulator.json /app/testdata/telco/simulator.json
RUN mkdir -p /data && chown nonroot:nonroot /data
USER nonroot:nonroot
HEALTHCHECK --interval=15s --timeout=2s --start-period=20s --retries=3 CMD ["/health-probe"]
EXPOSE 8080
ENTRYPOINT ["/kinga"]
