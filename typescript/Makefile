.PHONY: build test install

install:
	cd web && npm install

build:
	cd web && npm run build

# The page has no unit tests, and this is the check that can fail: `npm run build`
# is `tsc --noEmit && vite build`, so an identifier that does not resolve or an
# import that is gone stops it. It replaced `npm test --if-present`, which found
# no script, printed nothing and passed - a gate that could not say red.
test:
	cd web && npm run build
