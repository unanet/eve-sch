# eve-sch

[![pipeline status](https://gitlab.unanet.io/devops/eve-sch/badges/master/pipeline.svg)](https://gitlab.unanet.io/devops/eve-sch/-/commits/master) [![Bugs](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=bugs)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Code Smells](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=code_smells)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Coverage](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=coverage)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Lines of Code](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=ncloc)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Maintainability Rating](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=sqale_rating)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Quality Gate Status](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=alert_status)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Reliability Rating](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=reliability_rating)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Security Rating](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=security_rating)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Technical Debt](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=sqale_index)](https://sonarqube.unanet.io/dashboard?id=eve-sch) [![Vulnerabilities](https://sonarqube.unanet.io/api/project_badges/measure?project=eve-sch&metric=vulnerabilities)](https://sonarqube.unanet.io/dashboard?id=eve-sch)

- scheduler will be only thing we can assume has access to vault..
- provision will go through scheduler
- there will a scheduler per cluster
- there will be an sqs queue per cluster
- we need to figure out how to authenticate scheduler with s3/sqs outside of AWS
- the scheduler will be given a plan (via s3 through the queue) specific to a namespace and the cluster the scheduler lives in
- the scheduler will need to translate some sort of templating language for vault secrets
- the scheduler will need to be able to deploy to the kubernetes cluster it is in given the deployment plan
- the scheduler will need to be able to trigger an azure function with a response of success/failure
