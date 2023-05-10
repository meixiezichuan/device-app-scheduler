ARG ARCH
FROM golang:1.16.10

WORKDIR /
COPY . .
ARG ARCH
ARG RELEASE_VERSION
RUN RELEASE_VERSION=${RELEASE_VERSION} make build-scheduler.$ARCH

FROM $ARCH/alpine:3.12

COPY --from=0 bin/da-scheduler /bin/kube-scheduler

WORKDIR /bin
CMD ["kube-scheduler"]
