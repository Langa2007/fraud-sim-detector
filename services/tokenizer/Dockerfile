FROM rust:1.85-bookworm AS build
WORKDIR /src
COPY services/tokenizer/Cargo.toml ./Cargo.toml
COPY services/tokenizer/Cargo.lock ./Cargo.lock
COPY services/tokenizer/src ./src
RUN cargo build --locked --release

FROM debian:bookworm-slim
RUN useradd --system --uid 10001 tokenizer
COPY --from=build /src/target/release/kinga-tokenizer /usr/local/bin/kinga-tokenizer
USER 10001
EXPOSE 8091
ENTRYPOINT ["/usr/local/bin/kinga-tokenizer"]
