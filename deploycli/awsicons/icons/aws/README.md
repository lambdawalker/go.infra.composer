# AWS service icons

These fifteen SVGs are the AWS service icons downloaded from
[AWS Icons](https://aws-icons.com/), which identifies the
[AWS Architecture Icons](https://aws.amazon.com/architecture/icons/) package as
their source. The artwork is owned by Amazon Web Services, Inc.; consult AWS's
icon terms before reusing it elsewhere.

The local wizard serves these files itself. `aws-icons.js` maps known Pulumi and
Composer-recorded AWS resource families to the bundled service icon;
`workspace.js` uses its neutral symbol for other providers and components.
No icon request goes to the catalog at runtime.
