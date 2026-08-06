# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e
FROM node:24-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS deps
WORKDIR /app
RUN npm install --global pnpm@10.32.1
COPY package.json pnpm-workspace.yaml ./
COPY pnpm-lock.yaml ./
COPY patches/minimatch@3.1.5.patch patches/minimatch@3.1.5.patch
COPY apps/web/package.json apps/web/package.json
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --filter @cineweave/web --frozen-lockfile

FROM deps AS build
ARG CINEWEAVE_WEB_EDITION=community
ARG NEXT_PUBLIC_API_BASE_URL=
ARG NEXT_PUBLIC_REALTIME_URL=
ARG S3_PUBLIC_ENDPOINT=http://localhost:19290
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL
ENV NEXT_PUBLIC_REALTIME_URL=$NEXT_PUBLIC_REALTIME_URL
ENV S3_PUBLIC_ENDPOINT=$S3_PUBLIC_ENDPOINT
COPY --from=deps /app/node_modules ./node_modules
COPY --from=deps /app/apps/web/node_modules ./apps/web/node_modules
COPY . .
RUN test "${CINEWEAVE_WEB_EDITION}" = "community"
RUN pnpm --filter @cineweave/web build

FROM node:24-alpine@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS runtime
WORKDIR /app
LABEL org.cineweave.edition="community"
ARG NEXT_PUBLIC_API_BASE_URL=
ARG NEXT_PUBLIC_REALTIME_URL=
ARG S3_PUBLIC_ENDPOINT=http://localhost:19290
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL
ENV NEXT_PUBLIC_REALTIME_URL=$NEXT_PUBLIC_REALTIME_URL
ENV S3_PUBLIC_ENDPOINT=$S3_PUBLIC_ENDPOINT
RUN addgroup -S cineweave && adduser -S cineweave -G cineweave
COPY --from=build --chown=cineweave:cineweave /app/apps/web/.next/standalone ./
COPY --from=build --chown=cineweave:cineweave /app/apps/web/.next/static ./apps/web/.next/static
COPY --from=build --chown=cineweave:cineweave /app/apps/web/public ./apps/web/public
USER cineweave
EXPOSE 3000
CMD ["node", "apps/web/server.js"]
