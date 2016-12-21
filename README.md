# Dependency Injector
A dependency injector which extends facebook injector functionatlity. The injector supports life-cycle pattern what allows to initialize and de-initialize components in a specific order. 

Typical usage workflow looks like this:

```
import "github.com/jrivets/inject"
...
func main() {
	injector := inject.NewInjector()
	defer injector.Shutdown()
	
	injector.Register(&inject.Component{comp1, "comp1"}, &inject.Component{comp2, "comp2"} ...)
	injector.Construct()
	... 
}
```
Injections rules are the same as described in https://godoc.org/github.com/facebookgo/inject


## LifeCycler interface
The inject.LifeCycler component should support 3 functions: 

```
type LifeCycler interface {
	DiPhase() int
	DiInit() error
	DiShutdown()
}
```

`DiPhase()` returns an initialization phase for the component (components with lower values are initialized before components with higher values)
`DiInit()` the component initialized. Will be called when all fields are injected.
`DiShutdown()` the component de-initializer. It will be called in `injctor.Shutdown()` call. The de-initialization process is done in reverse order - components with higher phase values are de-initialized before components with lower ones. 

If a life-cycler panics or reports an error during initialization, all previously initialized components will be de-initialized in the reverse or their initialization order and `injector.Construct()` will panic then. 