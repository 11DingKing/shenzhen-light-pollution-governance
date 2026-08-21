FROM golang:1.26

ARG GOPROXY=https://proxy.golang.org,direct
ENV GOTOOLCHAIN=local
ENV GOPROXY=${GOPROXY}

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build ./...

CMD ["bash"]
