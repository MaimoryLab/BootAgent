export function isConverterID(id: string): boolean {
  return id.startsWith("bootagent-converter-") || id.startsWith("bootagent_converter_");
}
