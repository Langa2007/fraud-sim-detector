FROM rust:1.90-bookworm AS build
WORKDIR /src
COPY engine/Cargo.toml engine/Cargo.lock* ./engine/
COPY engine/src ./engine/src
RUN cargo build --manifest-path engine/Cargo.toml --release
FROM debian:bookworm-slim
RUN useradd --system --uid 10001 kinga
WORKDIR /app
COPY --from=build /src/engine/target/release/kinga-engined /usr/local/bin/kinga-engined
COPY policies/default.json /app/policies/default.json
USER kinga
ENV KINGA_POLICY_PATH=/app/policies/default.json
EXPOSE 8090
ENTRYPOINT ["/usr/local/bin/kinga-engined"]
