# CineWeave Remote MinIO

This stack runs a persistent MinIO API for CineWeave and does not publish the MinIO console.

```bash
install -d -m 0700 /opt/cineweave-minio /srv/cineweave-minio/data
cd /opt/cineweave-minio
cp .env.example .env
chmod 0600 .env
docker compose up -d
docker compose ps
```

The bootstrap service creates the `cineweave` bucket and a bucket-scoped `cineweave-rw` application policy. CineWeave should use the application access key, not the MinIO root credentials.

For an internet-facing production deployment, terminate TLS in front of port `19290` and set `S3_PUBLIC_ENDPOINT` to that HTTPS origin. The MinIO console on port `9001` remains internal.
