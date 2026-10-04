# Zero config

The project is developed in a fashion that ensures least required configuration.
Everything that can have a sane default value must have a sane default value assuming portable deploy.
Overridable configs are added when strictly necessary on per case basis.
Configs that cannot be known ahead of time, such as the address of the model server, are required inputs.
They never default to what a developer's machine happens to have, like `localhost:11434`.
A missing input is an error with a clear message, never a silent guess.
