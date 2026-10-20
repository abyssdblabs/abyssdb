# AbyssDB in a scratch image: one static binary and nothing else.
# Build the binaries first with scripts/release/build.sh.
FROM scratch
ARG TARGETARCH
COPY dist/docker/linux-${TARGETARCH}/abyssdb /abyssdb

LABEL org.opencontainers.image.title="AbyssDB" \
      org.opencontainers.image.description="Infinitely fast. Perfectly consistent. Zero-knowledge." \
      org.opencontainers.image.source="https://github.com/noctarius/abyssdb" \
      org.opencontainers.image.url="https://abyssdb.xyz" \
      org.opencontainers.image.licenses="MIT"

# nobody, as every AbyssDB session already is.
USER 65534:65534
EXPOSE 5432 4040 9187
ENTRYPOINT ["/abyssdb"]
CMD ["serve", "--pg-addr", "0.0.0.0:5432", "--awp-addr", "0.0.0.0:4040", "--metrics-addr", "0.0.0.0:9187"]
