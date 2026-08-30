# The whole reference comes from one ARG default, so the ARG owns the bytes an
# update would rewrite and the reference resolves there.
ARG BASE_IMAGE=debian:13-slim

# Only a fragment comes from the ARG. The expansion is debian:12-slim, which is
# written nowhere, so this must stay unresolved rather than tempt an update to
# splice a bare tag.
ARG DEBIAN_VERSION=12

# No default at all: docker build would demand --build-arg for it.
ARG UNSET_IMAGE

FROM "${BASE_IMAGE}" AS first
RUN true

# A second stage on the same ARG. The ARG is reported once; both FROMs point
# at it.
FROM ${BASE_IMAGE} AS second
RUN true

FROM debian:${DEBIAN_VERSION}-slim AS fragment
RUN true

FROM ${UNSET_IMAGE} AS unset
RUN true
