#!/usr/bin/env sh
set -eu

image=${1:?usage: verify-image.sh IMAGE_REFERENCE}

docker run --rm --entrypoint sh "$image" -ec '
  command -v cdml
  cdml --help >/dev/null
  command -v ffmpeg
  python -c "import torch; print(torch.__version__)"
'
