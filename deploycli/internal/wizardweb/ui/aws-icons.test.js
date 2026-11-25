"use strict";

const assert = require("node:assert/strict");
const test = require("node:test");
const {awsIconFor} = require("./aws-icons.js");

test("maps the email verification and discovery AWS resource families", () => {
  const cases = {
    "aws:acm/certificate:Certificate": "acm",
    "aws:apigatewayv2/api:Api": "api-gateway",
    "aws:cognito/userPool:UserPool": "cognito",
    "aws:dynamodb/table:Table": "dynamodb",
    "aws:iam/rolePolicy:RolePolicy": "iam",
    "aws:kms/key:Key": "kms",
    "aws:lambda/function:Function": "lambda",
    "aws:route53/record:Record": "route53",
    "aws:secretsmanager/secretVersion:SecretVersion": "secretsmanager",
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
  const shortened = {
    "aws:acm:Certificate": "acm", "aws:apigatewayv2:Api": "api-gateway",
    "aws:cognito:UserPool": "cognito", "aws:dynamodb:Table": "dynamodb",
    "aws:iam:Role": "iam", "aws:kms:Key": "kms", "aws:lambda:Function": "lambda",
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
    "cloudflare:index/dnsRecord:DnsRecord", "apexfission:email-verification:TestHost",
    "aws:unknown/resource:Resource", "aws:constructor/resource:Resource",
    "aws:s3/../../../secret:Bucket", "aws:s3/stateBucket/extra"]) {
    assert.equal(awsIconFor(type), "", type);
  }
});
