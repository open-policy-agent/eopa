all: transform overlap no-roots prefix

clean:
	rm -f transform overlap no-roots prefix

%: %-source
	opa build -b $<
	mv bundle.tar.gz $@
