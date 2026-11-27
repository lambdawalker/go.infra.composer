"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const {awsIconFor} = require("../../../awsicons/aws-icons.js");

test("maps the email verification and discovery AWS resource families", () => {
  const cases = {
    "aws:acm/certificate:Certificate": "acm",
    "aws:apigatewayv2/apiMapping:ApiMapping": "api-gateway-mapping",
    "aws:apigatewayv2:ApiMapping": "api-gateway-mapping",
    "aws:apigatewayv2:Stage": "api-gateway",
    "aws:apigatewayv2/integration:Integration": "api-gateway-integration",
    "aws:apigateway:Integration": "api-gateway-integration",
    "aws:apigatewayv2:IntegrationResponse": "api-gateway",
    "aws:apigatewayv2/route:Route": "api-gateway-route",
    "aws:apigatewayv2:Route": "api-gateway-route",
    "aws:apigatewayv2/api:Api": "api-gateway",
    "aws:cognito/userPool:UserPool": "cognito",
    "aws:dynamodb/table:Table": "dynamodb",
    "aws:iam/rolePolicy:RolePolicy": "iam",
    "aws:kms/key:Key": "kms",
    "aws:lambda/function:Function": "lambda",
    "aws:lambda/permission:Permission": "lambda-permission",
    "aws:route53/record:Record": "route53",
    "aws:secretsmanager/secretVersion:SecretVersion": "secret-version",
    "aws:secretsmanager:SecretVersion": "secret-version",
    "aws:ses/emailIdentity:EmailIdentity": "ses",
    "aws:cloudwatch/logGroup:LogGroup": "cloudwatch",
    "aws:cloudwatch/eventRule:EventRule": "eventbridge",
    "aws:cloudwatch/eventTarget:EventTarget": "eventbridge"
  };
  for (const [type, icon] of Object.entries(cases)) {
    assert.equal(awsIconFor(type), `/icons/aws/${icon}.svg`, type);
  }
});

test("maps recorded state buckets and legacy abbreviated AWS types", () => {
  for (const type of ["aws:s3/stateBucket", "aws:s3/bucketV2:BucketV2", "aws:s3:Bucket",
    "aws:s3/bucketObjectv2:BucketObjectv2", "aws:s3/bucketPolicy:BucketPolicy"]) {
    assert.equal(awsIconFor(type), "/icons/aws/s3.svg", type);
  }
  assert.equal(awsIconFor("aws:cloudwatch:EventRule"), "/icons/aws/eventbridge.svg");
  assert.equal(awsIconFor("aws:apigatewayv2:Authorizer", "jwt"), "/icons/aws/api-gateway-jwt.svg");
  assert.equal(awsIconFor("aws:apigatewayv2:Authorizer"), "/icons/aws/api-gateway.svg");
  const shortened = {
    "aws:lambda:Permission": "lambda-permission",
    "aws:acm:Certificate": "acm", "aws:apigatewayv2:Api": "api-gateway",
    "aws:cognito:UserPool": "cognito", "aws:dynamodb:Table": "dynamodb",
    "aws:iam:Role": "iam-role", "aws:acm:CertificateValidation": "acm-validation", "aws:kms:Key": "kms", "aws:lambda:Function": "lambda",
    "aws:route53:Record": "route53", "aws:secretsmanager:Secret": "secretsmanager",
    "aws:ses:EmailIdentity": "ses", "aws:cloudwatch:LogGroup": "cloudwatch",
    "aws:cloudwatch:EventTarget": "eventbridge"
  };
  for (const [type, icon] of Object.entries(shortened)) {
    assert.equal(awsIconFor(type), `/icons/aws/${icon}.svg`, type);
  }
});

test("keeps custom, other-provider, and malformed types on the neutral fallback", () => {
  for (const type of ["", "pulumi:pulumi:Stack", "random:index/randomBytes:RandomBytes",
    "cloudflare:index/zone:Zone", "apexfission:email-verification:TestHost",
    "aws:unknown/resource:Resource", "aws:constructor/resource:Resource",
    "aws:s3/../../../secret:Bucket", "aws:s3/stateBucket/extra"]) {
    assert.equal(awsIconFor(type), "", type);
  }
});
