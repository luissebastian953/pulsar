# SPA build (ssr:false). The client bundle calls core directly from the
# browser, so NUXT_PUBLIC_API_BASE is baked at BUILD time (a runtime env has
# no effect on an already-built SPA). Default targets the host-published core.
FROM node:22-alpine AS build
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
ARG NUXT_PUBLIC_API_BASE=http://localhost:8080
ENV NUXT_PUBLIC_API_BASE=$NUXT_PUBLIC_API_BASE
RUN pnpm build

FROM node:22-alpine AS run
WORKDIR /app
COPY --from=build /app/.output ./.output
ENV PORT=3000 HOST=0.0.0.0
EXPOSE 3000
CMD ["node", ".output/server/index.mjs"]
