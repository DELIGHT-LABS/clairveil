# Clairveil Reference App

이 디렉터리는 `clairveild`가 사용하는 최소 Cosmos SDK reference app을 담습니다.

이 reference app의 목적은 reusable `x/privacy` 모듈을 실제 chain host 위에서 검증하는 것입니다. Downstream target project의 features, validator 운영 설정을 이 repo에 섞지 않고도 local node, e2e smoke, tutorial을 실행할 수 있게 해줍니다.

Production app으로 그대로 쓰기 위한 구조가 아니라, Clairveil privacy core를 포크하거나 import하는 팀이 통합 전에 동작을 확인하는 기준 구현입니다.
