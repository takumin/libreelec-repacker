#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(cd "$(dirname "${SCRIPT_DIR}/../..")" && pwd)"

cd "${PROJECT_DIR}"

ORIGIN_OWNER="takumin"
ORIGIN_AUTHOR_NAME="Takumi Takahashi"
ORIGIN_REPOSITORY="boilerplate-golang-cli"
ORIGIN_DESCRIPTION="Boilerplate Golang CLI Tool"

GITHUB_NAME_WITH_OWNER="$(gh repo view --json nameWithOwner --jq '.nameWithOwner')"

GITHUB_OWNER="${GITHUB_NAME_WITH_OWNER%/*}"
GITHUB_REPOSITORY="${GITHUB_NAME_WITH_OWNER##*/}"
GITHUB_DESCRIPTION="$(gh repo view --json description --jq '.description')"
GITHUB_DESCRIPTION="${GITHUB_DESCRIPTION/\&/\\&}"

GITHUB_AUTHOR_NAME="$(gh api "users/${GITHUB_OWNER}" --jq '.name')"
[[ -z "${GITHUB_AUTHOR_NAME}" ]] && GITHUB_AUTHOR_NAME="${GITHUB_OWNER}"

ORIGIN_URL="github.com/${ORIGIN_OWNER}/${ORIGIN_REPOSITORY}"
GITHUB_URL="github.com/${GITHUB_OWNER}/${GITHUB_REPOSITORY}"

go mod edit -module "${GITHUB_URL}"
go-imports-rename -s "${ORIGIN_URL} => ${GITHUB_URL}"

sed -i -E "s@appName.*string.*=.*// ###BOILERPLATE_APP_NAME###@appName string = \"${GITHUB_REPOSITORY}\"@" internal/metadata/metadata.go
sed -i -E "s@appDesc.*string.*=.*// ###BOILERPLATE_APP_DESC###@appDesc string = \"${GITHUB_DESCRIPTION}\"@" internal/metadata/metadata.go
sed -i -E "s@authorName.*string.*=.*// ###BOILERPLATE_AUTHOR_NAME###@authorName string = \"${GITHUB_AUTHOR_NAME}\"@" internal/metadata/metadata.go

gofmt -w .

sed -i -E "s@${ORIGIN_URL}@${GITHUB_URL}@g" README.md
sed -i -E "s@${ORIGIN_OWNER}@${GITHUB_OWNER}@g" README.md
sed -i -E "s@${ORIGIN_AUTHOR_NAME}@${GITHUB_AUTHOR_NAME}@g" README.md
sed -i -E "s@${ORIGIN_REPOSITORY}@${GITHUB_REPOSITORY}@g" README.md
sed -i -E "s@${ORIGIN_DESCRIPTION}@${GITHUB_DESCRIPTION}@g" README.md

sed -i -E "s@${ORIGIN_URL}@${GITHUB_URL}@" CODE_OF_CONDUCT.md

sed -i -E "s@${ORIGIN_URL}@${GITHUB_URL}@" CONTRIBUTING.md

sed -i -E "s@${ORIGIN_URL}@${GITHUB_URL}@" book.toml
sed -i -E "s@${ORIGIN_OWNER}@${GITHUB_OWNER}@" book.toml
sed -i -E "s@${ORIGIN_AUTHOR_NAME}@${GITHUB_AUTHOR_NAME}@" book.toml
sed -i -E "s@${ORIGIN_REPOSITORY}@${GITHUB_REPOSITORY}@" book.toml
sed -i -E "s@${ORIGIN_DESCRIPTION}@${GITHUB_DESCRIPTION}@" book.toml

sed -i -E "s@\[yyyy\]@$(date "+%Y")@" LICENSE
sed -i -E "s@\[name of copyright owner\]@${GITHUB_AUTHOR_NAME}@" LICENSE
