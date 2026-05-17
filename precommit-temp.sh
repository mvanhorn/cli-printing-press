files="$(printf "%s\n" internal/generator/generator.go internal/generator/plan_generate.go | grep -v "^testdata/golden/expected/" || true)"
test -z "$files" || { printf "%s\n" "$files" | xargs gofmt -w; printf "%s\n" "$files" | xargs git add; }
