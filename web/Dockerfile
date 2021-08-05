FROM node:lts-slim AS build
WORKDIR /app/src
COPY . .

# Fetch dependencies
RUN apt update && \
    apt install -y jq && \
    apt clean

# Perform an audit on dependencies to ensure there are no significant vulnerabilities
#RUN test "$(yarn audit --level critical --json | jq '.data.vulnerabilities | .moderate + .high + .critical')" -eq 0

# Build
RUN yarn install && \
    yarn build --verbose

# Copy to nginx deployment container
FROM nginx:1.16.1-alpine
COPY --from=build /app/src/build /usr/share/nginx/html
COPY deploy/default.conf /etc/nginx/conf.d/default.conf

EXPOSE 3000
CMD ["nginx", "-g", "daemon off;"]