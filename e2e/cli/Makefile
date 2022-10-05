DOCKER:=docker run -v $$(pwd):/w -w /w $(IMAGE)

.PHONY: test
test:
	$(DOCKER) eval -fpretty --metrics 'opa.runtime()'
	$(DOCKER) test -v .
	$(DOCKER) test --bench .
	$(DOCKER) bench true
