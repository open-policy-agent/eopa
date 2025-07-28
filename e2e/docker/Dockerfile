# We need the debug image here because "RUN eopa version" needs
# a shell of some sort.
FROM ko.local/eopa:edge-debug AS builder
RUN eopa version
RUN /bin/eopa version
RUN opa version
RUN /bin/opa version
RUN /opa version
# test presence of some debug utils
RUN type kcat
RUN type curl
RUN type grpcurl
RUN type dig
RUN type ping
RUN type htop
RUN type strace
RUN type tcpdump
RUN type ldapsearch

# test that it also copies cleanly and can be used on alpine
FROM alpine:latest AS throwaway
COPY --from=builder /opa /bin/opa
RUN opa version

# test that we can copy eopa from the non-debug image
FROM alpine:latest
COPY --from=ko.local/eopa:edge /bin/eopa /bin/opa /opa /ko-app/eopa /bin/
# to ensure that "builder" is built
COPY --from=builder /bin/opa /tmp/
RUN diff /bin/eopa /bin/opa
RUN diff /bin/eopa /bin/eopa
