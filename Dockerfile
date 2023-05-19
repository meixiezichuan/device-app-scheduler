ARG ARCH
FROM golang:1.16.10

WORKDIR /
COPY go.mod go.mod
COPY go.sum go.sum
COPY vendor/ vendor/
#RUN GOPRIVATE="github.com/meixiezichuan/*" go mod download

COPY Makefile Makefile
COPY main.go main.go
COPY networkaware/ networkaware/

ARG ARCH
ARG RELEASE_VERSION
#RUN RELEASE_VERSION=${RELEASE_VERSION} make build-scheduler.$ARCH
RUN CGO_ENABLED=0 GOARCH=amd64 go build -o bin/da-scheduler main.go

FROM $ARCH/alpine:3.12

COPY --from=0 bin/da-scheduler /bin/kube-scheduler

WORKDIR /bin
CMD ["kube-scheduler"]
