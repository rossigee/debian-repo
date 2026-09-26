package tracing

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

// PackageRegistration traces a package registration operation
func PackageRegistration(ctx context.Context, suite, component, pkgName, version, arch string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "package.register",
		attribute.String("suite", suite),
		attribute.String("component", component),
		attribute.String("package", pkgName),
		attribute.String("version", version),
		attribute.String("architecture", arch),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}

// IndexMutation traces index add/remove operations
func IndexMutation(ctx context.Context, operation, suite, component, pkgName string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "index."+operation,
		attribute.String("suite", suite),
		attribute.String("component", component),
		attribute.String("package", pkgName),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}

// MinIOOperation traces MinIO storage operations
func MinIOOperation(ctx context.Context, operation, bucket, key string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "minio."+operation,
		attribute.String("bucket", bucket),
		attribute.String("object_key", key),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}

// GPGSign traces GPG signing operations
func GPGSign(ctx context.Context, signingType, keyID string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "gpg.sign",
		attribute.String("signing_type", signingType),
		attribute.String("key_id", keyID),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}

// Validation traces validation operations
func Validation(ctx context.Context, validationType string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "validate."+validationType,
		attribute.String("validation_type", validationType),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}

// Authentication traces auth operations
func Authentication(ctx context.Context, authType, identity string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "auth."+authType,
		attribute.String("auth_type", authType),
		attribute.String("identity", identity),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}

// Reconciliation traces index reconciliation
func Reconciliation(ctx context.Context) (context.Context, func(error, int)) {
	start := time.Now()
	ctx, span := StartSpan(ctx, "index.reconcile")

	return ctx, func(err error, discrepancies int) {
		if err != nil {
			RecordError(span, err)
		}
		SetAttribute(span, "discrepancies", discrepancies)
		SetAttribute(span, "duration_ms", time.Since(start).Milliseconds())
		span.End()
	}
}

// RenderMetadata traces Release/Packages rendering
func RenderMetadata(ctx context.Context, suite, component string) (context.Context, func(error)) {
	ctx, span := StartSpan(ctx, "metadata.render",
		attribute.String("suite", suite),
		attribute.String("component", component),
	)

	return ctx, func(err error) {
		if err != nil {
			RecordError(span, err)
		}
		span.End()
	}
}
