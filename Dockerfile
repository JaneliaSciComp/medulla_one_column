# ---- http Build ----
FROM golang:1.21 as builder
WORKDIR /go/src/github.com/JaneliaSciComp/
ENV CGO_ENABLED=0
# fetch source from github tag
RUN git clone --depth 1 --branch master https://github.com/JaneliaSciComp/medulla_one_column.git
WORKDIR /go/src/github.com/JaneliaSciComp/medulla_one_column
# run go build
RUN go build .

FROM alpine:latest
MAINTAINER flyem project team
LABEL maintainer="neuprint@janelia.hhmi.org"

WORKDIR /go/src/github.com/JaneliaSciComp/medulla_one_column/
COPY --from=builder /go/src/github.com/JaneliaSciComp/medulla_one_column/ .
EXPOSE 80
CMD ./medulla_one_column -http=:80
