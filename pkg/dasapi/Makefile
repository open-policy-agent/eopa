v3.json:
	curl -vLo $@ https://docs.styra.com/openapi/v3

generated.go: v3.json server.yml
	oapi-codegen -config server.yml $<
