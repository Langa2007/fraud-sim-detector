# Protobuf contracts

This directory is the Buf-managed starting point for service RPC contracts. The
initial definitions cover assessment, events, consortium indicators, tokenisation,
and telco signals. `schemas/events/assessment.schema.json` and the OpenAPI
document remain the active HTTP/JSON contracts until gateway and service
transports are migrated to gRPC.

Use `just gen` to generate Go stubs under the ignored `gen/go/` directory. Buf
lint, formatting, descriptor builds, and pull-request compatibility checks run
in CI.
