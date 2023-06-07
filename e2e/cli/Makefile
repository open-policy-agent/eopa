DOCKER:=docker run -e EOPA_LICENSE_TOKEN -v $$(pwd):/w -w /w --user root $(IMAGE)

.PHONY: test
test:
	$(DOCKER) version
	$(DOCKER) eval -fpretty --metrics 'opa.runtime()'
	$(DOCKER) test -v pol_test.rego
	$(DOCKER) test --bench pol_test.rego
	$(DOCKER) bench true
	$(DOCKER) build pol_test.rego
	$(DOCKER) bundle convert bundle.tar.gz enterprise-opa-bundle.tar.gz
	$(DOCKER) eval -fpretty -b enterprise-opa-bundle.tar.gz 'data.pkg'
	$(DOCKER) inspect enterprise-opa-bundle.tar.gz
	$(DOCKER) bundle dump enterprise-opa-bundle.tar.gz
