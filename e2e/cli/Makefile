DOCKER:=docker run -e STYRA_LOAD_LICENSE_TOKEN -v $$(pwd):/w -w /w --user root $(IMAGE)

.PHONY: test
test:
	$(DOCKER) version
	$(DOCKER) eval -fpretty --metrics 'opa.runtime()'
	$(DOCKER) test -v pol_test.rego
	$(DOCKER) test --bench pol_test.rego
	$(DOCKER) bench true
	$(DOCKER) build pol_test.rego
	$(DOCKER) bundle convert bundle.tar.gz load-bundle.tar.gz
	$(DOCKER) eval -fpretty -b load-bundle.tar.gz 'data.pkg'
	$(DOCKER) inspect load-bundle.tar.gz
	$(DOCKER) bundle dump load-bundle.tar.gz
