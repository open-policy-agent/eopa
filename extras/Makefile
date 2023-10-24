
.PHONY: build irdump irviz clean
build: irdump irviz

irdump:
	go build -o irdump cmd/irdump/main.go

irviz:
	go build -o irviz cmd/irviz/*.go

clean:
	rm -f irdump irviz