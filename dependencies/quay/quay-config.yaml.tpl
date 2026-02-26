BUILDLOGS_REDIS:
    host: quay-redis.quay
    port: 6379
CREATE_NAMESPACE_ON_PUSH: true
DATABASE_SECRET_KEY: ${DATABASE_SECRET_KEY}
DB_URI: postgresql://quay:${POSTGRES_PASSWORD}@quay-postgres.quay:5432/quay
DISTRIBUTED_STORAGE_CONFIG:
    default:
        - LocalStorage
        - storage_path: /datastorage/registry
DISTRIBUTED_STORAGE_DEFAULT_LOCATIONS: []
DISTRIBUTED_STORAGE_PREFERENCE:
    - default
FEATURE_MAILING: false
FEATURE_USER_INITIALIZE: true
PREFERRED_URL_SCHEME: https
SECRET_KEY: ${SECRET_KEY}
SERVER_HOSTNAME: quay-service.quay
SETUP_COMPLETE: true
SUPER_USERS:
    - quayadmin
USER_EVENTS_REDIS:
    host: quay-redis.quay
    port: 6379
