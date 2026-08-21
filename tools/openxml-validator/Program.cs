using System.Text.Json;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Validation;

var result = new ValidationResult();
try
{
    using var document = WordprocessingDocument.Open(args.Single(), false);
    result.Errors = new OpenXmlValidator(FileFormatVersions.Microsoft365)
        .Validate(document)
        .Take(100)
        .Select(error => new ValidationError(
            Safe(() => error.Description),
            Safe(() => error.Path?.XPath ?? error.Part?.Uri.ToString()),
            Safe(() => error.Id)))
        .ToList();
    result.Ok = result.Errors.Count == 0;
}
catch (Exception error)
{
    result.Errors.Add(new ValidationError(error.Message, "", "Exception"));
}

Console.WriteLine(JsonSerializer.Serialize(result, new JsonSerializerOptions
{
    PropertyNamingPolicy = JsonNamingPolicy.CamelCase
}));
return result.Ok ? 0 : 1;

static string Safe(Func<string?> value)
{
    try { return value() ?? ""; }
    catch { return ""; }
}

sealed class ValidationResult
{
    public bool Ok { get; set; }
    public List<ValidationError> Errors { get; set; } = [];
}

sealed record ValidationError(string Description, string Path, string Id);
