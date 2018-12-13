FROM golang:latest

COPY . $GOPATH/src/github.com/heysphere/segment-proxy
WORKDIR $GOPATH/src/github.com/heysphere/segment-proxy
RUN go get github.com/tools/godep
RUN godep restore
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o bin/segment-proxy

##################

FROM golang:alpine

# add certificates for remote TLS calls
RUN apk add ca-certificates
WORKDIR /root
COPY --from=0 $GOPATH/src/github.com/heysphere/segment-proxy/bin/segment-proxy .

EXPOSE 8080
CMD ["./segment-proxy"]
