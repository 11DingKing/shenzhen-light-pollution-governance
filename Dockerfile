FROM golang:1.26 AS builder
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOTOOLCHAIN=local
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /lightgovernance ./cmd/lightgovernance
RUN CGO_ENABLED=0 go build -o /hubctl ./cmd/hubctl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /lightgovernance /lightgovernance
COPY --from=builder /hubctl /hubctl
EXPOSE 49820
ENTRYPOINT ["/lightgovernance"]
