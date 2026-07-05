FROM node:24-alpine AS deps
WORKDIR /app
RUN corepack enable
COPY package.json pnpm-workspace.yaml ./
COPY pnpm-lock.yaml ./
COPY apps/web/package.json apps/web/package.json
RUN pnpm install --filter @cineweave/web --frozen-lockfile

FROM node:24-alpine AS build
WORKDIR /app
RUN corepack enable
ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
ARG NEXT_PUBLIC_REALTIME_URL=http://localhost:8081/api/realtime/events
ARG S3_PUBLIC_ENDPOINT=http://localhost:9000
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL
ENV NEXT_PUBLIC_REALTIME_URL=$NEXT_PUBLIC_REALTIME_URL
ENV S3_PUBLIC_ENDPOINT=$S3_PUBLIC_ENDPOINT
COPY --from=deps /app/node_modules ./node_modules
COPY --from=deps /app/apps/web/node_modules ./apps/web/node_modules
COPY . .
RUN pnpm --filter @cineweave/web build

FROM node:24-alpine AS runtime
WORKDIR /app
ARG NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
ARG NEXT_PUBLIC_REALTIME_URL=http://localhost:8081/api/realtime/events
ARG S3_PUBLIC_ENDPOINT=http://localhost:9000
ENV NODE_ENV=production
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL
ENV NEXT_PUBLIC_REALTIME_URL=$NEXT_PUBLIC_REALTIME_URL
ENV S3_PUBLIC_ENDPOINT=$S3_PUBLIC_ENDPOINT
RUN corepack enable
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/pnpm-workspace.yaml ./pnpm-workspace.yaml
COPY --from=build /app/apps/web ./apps/web
COPY --from=build /app/node_modules ./node_modules
COPY --from=build /app/apps/web/node_modules ./apps/web/node_modules
EXPOSE 3000
CMD ["pnpm", "--filter", "@cineweave/web", "start"]
