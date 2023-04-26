all: transform overlap no-roots prefix

%: %-source
	opa build -b $<
	mv bundle.tar.gz $@
