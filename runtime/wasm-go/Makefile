APP=qqq
OUT=build/$(APP).wasm

build:
	mkdir -p build
	tinygo build -o $(OUT) -target=wasi .

clean:
	rm -rf build
