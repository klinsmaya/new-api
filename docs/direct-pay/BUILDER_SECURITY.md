# Builder security patch

Only the Go builder stage moves from 1.26.1 to 1.26.8 within the same minor release. Business baseline, Bun builder, final Debian stage and release targets are unchanged.

Official release notes: https://go.dev/doc/devel/release#go1.26.0 . The original payment-path govulncheck findings and fixed versions are preserved in evidence/dependency-vulnerabilities.log; exact CVE aliases retrieved from vuln.go.dev are in evidence/go-cve-mapping.txt. Latest local 1.26.8 payment-path scan is clean for reachable findings; this is not a whole-image certification.

Pinned multi-architecture index digest `sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c` was independently obtained as the official Docker Hub library/golang tag API digest and by hashing the public ECR Docker Official Images mirror response. Full Docker Hub tag API result is retained in evidence/go-builder-digest.txt. Registry manifest pulls hit anonymous rate limits; no registry credentials or host configuration were changed.

Local Go 1.26.8 root compilation and regression evidence precede this pin. Full Docker image build is separately tracked and must not be inferred from a successful binary build.
