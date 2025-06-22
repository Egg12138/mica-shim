# Progress


* Shim v2 API 实现 OK
* hello world, call, (80%, 传送层)

TODO


* 版本
*  containerd 1.7是containerd v1的末版本，1.7内部出现了明显的API变动，下一步先调整API到1.7.3之后的API
* libmica接口暴露调整为 Create, Stop, Rm, Delete ，其他都改为private
* package logger 调整
* replace all Unix process handler ==> rtos process monitor

TYPOS:
* micad会先响应一个"No such file"?
BUG
* fix mock_micad memory leaking...




# FUTURE
* containerd 2.0 (shim-v3)

TODO

* using XMake to manage the building system