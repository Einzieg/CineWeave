FROM node:24-alpine AS deps
WORKDIR /app
RUN npm install --global pnpm@10.32.1
COPY package.json pnpm-workspace.yaml ./
COPY pnpm-lock.yaml ./
COPY patches/minimatch@3.1.5.patch patches/minimatch@3.1.5.patch
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --filter @cineweave/web --frozen-lockfile

FROM deps AS build
ARG NEXT_PUBLIC_API_BASE_URL=
ARG NEXT_PUBLIC_REALTIME_URL=
ARG S3_PUBLIC_ENDPOINT=http://localhost:19290
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL
ENV NEXT_PUBLIC_REALTIME_URL=$NEXT_PUBLIC_REALTIME_URL
ENV S3_PUBLIC_ENDPOINT=$S3_PUBLIC_ENDPOINT
COPY --from=deps /app/node_modules ./node_modules
COPY --from=deps /app/apps/web/node_modules ./apps/web/node_modules
COPY . .
RUN pnpm --filter @cineweave/web build

FROM node:24-alpine AS runtime
WORKDIR /app
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
