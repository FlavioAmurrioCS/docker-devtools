FROM scratch AS tools
# Never reached from the final stage, so buildkit never reads vendor/ from the
# context and never transfers it.
COPY vendor /vendor

FROM scratch AS final
COPY app /app
