"use strict";

// Only known resource families resolve to bundled provider icons. The
// returned URL is fixed by this table, never interpolated from a resource name.
(() => {
  const services = Object.freeze({
    acm: "acm", apigateway: "api-gateway", apigatewayv2: "api-gateway",
    cognito: "cognito", dynamodb: "dynamodb", iam: "iam", kms: "kms",
    lambda: "lambda", neptune: "neptune", rds: "rds", route53: "route53", s3: "s3",
    secretsmanager: "secretsmanager", ses: "ses"
  });

  function awsIconFor(type, variant) {
    // Pulumi types end in :Kind; locally recorded resources such as the state
    // bucket can use aws:s3/stateBucket, and older records use aws:s3:Bucket.
    if (type === "cloudflare:index/dnsRecord:DnsRecord" || type === "cloudflare:DnsRecord" ||
        type === "cloudflare:index/record:Record") return "/icons/cloudflare/dns.svg";
    const match = /^aws:([a-z0-9]+)(?:\/([A-Za-z0-9]+)(?::([A-Za-z0-9]+))?|:([A-Za-z0-9]+))$/.exec(type);
    if (!match) return "";
    const [, service] = match;
    const resource = match[2] || match[4];
    if ((service === "apigateway" || service === "apigatewayv2") && /^integration$/i.test(resource)) return "/icons/aws/api-gateway-integration.svg";
    if (service === "apigatewayv2" && /^apiMapping$/i.test(resource)) return "/icons/aws/api-gateway-mapping.svg";
    if (service === "apigatewayv2" && /^route$/i.test(resource)) return "/icons/aws/api-gateway-route.svg";
    if (service === "secretsmanager" && /^secretVersion$/i.test(resource)) return "/icons/aws/secret-version.svg";
    if (service === "iam" && /^role$/i.test(resource)) return "/icons/aws/iam-role.svg";
    if (service === "acm" && /^certificateValidation$/i.test(resource)) return "/icons/aws/acm-validation.svg";
    if (service === "apigatewayv2" && /^authorizer$/i.test(resource) && variant === "jwt") return "/icons/aws/api-gateway-jwt.svg";
    if (service === "lambda" && /^permission$/i.test(resource)) return "/icons/aws/lambda-permission.svg";
    const icon = service === "cloudwatch" ?
      (/^event(Rule|Target)$/i.test(resource) ? "eventbridge" : "cloudwatch") :
      (Object.hasOwn(services, service) ? services[service] : "");
    return icon ? "/icons/aws/" + icon + ".svg" : "";
  }

  if (typeof window !== "undefined") window.wizardAWSIconFor = awsIconFor;
  if (typeof module !== "undefined" && module.exports) module.exports = {awsIconFor};
})();
