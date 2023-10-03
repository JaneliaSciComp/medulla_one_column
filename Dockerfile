# ---- http Build ----
FROM golang:1.21
WORKDIR /go/src/github.com/JaneliaSciComp/
# fetch source from github tag
RUN git clone --depth 1 --branch master https://github.com/JaneliaSciComp/medulla_one_column.git
WORKDIR /go/src/github.com/JaneliaSciComp/medulla_one_column
# run go build
RUN go build .
CMD ./medulla_one_column -http=:80
