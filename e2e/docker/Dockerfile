# We need the debug image here because "RUN eopa version" needs
# a shell of some sort.
FROM ko.local/enterprise-opa-private:edge-debug AS builder
RUN eopa version
RUN /bin/eopa version
RUN opa version
RUN /bin/opa version
RUN /opa version

# test that it also copies cleanly and can be used on alpine
FROM alpine:latest AS throwaway
COPY --from=builder /opa /bin/opa
RUN opa version

# test that we can copy eopa from the non-debug image
FROM alpine:latest
COPY --from=ko.local/enterprise-opa-private:edge /bin/eopa /bin/opa /opa /ko-app/enterprise-opa-private /bin/
RUN diff /bin/eopa /bin/opa
RUN diff /bin/eopa /bin/enterprise-opa-private
