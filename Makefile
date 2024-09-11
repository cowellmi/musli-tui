windows:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -o musli.exe cmd/musli/main.go

clean:
	rm -f musli.exe